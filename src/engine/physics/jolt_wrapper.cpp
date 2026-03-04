/******************************************************************************/
/* jolt_wrapper.cpp                                                           */
/******************************************************************************/
/*                            This file is part of                            */
/*                                KAIJU ENGINE                                */
/*                          https://kaijuengine.com/                          */
/******************************************************************************/
/* MIT License                                                                */
/*                                                                            */
/* Copyright (c) 2023-present Kaiju Engine authors (AUTHORS.md).              */
/* Copyright (c) 2015-present Brent Farris.                                   */
/*                                                                            */
/* May all those that this source may reach be blessed by the LORD and find   */
/* peace and joy in life.                                                     */
/* Everyone who drinks of this water will be thirsty again; but whoever       */
/* drinks of the water that I will give him shall never thirst; John 4:13-14  */
/*                                                                            */
/* Permission is hereby granted, free of charge, to any person obtaining a    */
/* copy of this software and associated documentation files (the "Software"), */
/* to deal in the Software without restriction, including without limitation  */
/* the rights to use, copy, modify, merge, publish, distribute, sublicense,   */
/* and/or sell copies of the Software, and to permit persons to whom the      */
/* Software is furnished to do so, subject to the following conditions:       */
/*                                                                            */
/* The above copyright notice and this permission notice shall be included in */
/* all copies or substantial portions of the Software.                        */
/*                                                                            */
/* THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS    */
/* OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF                 */
/* MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.     */
/* IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY       */
/* CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT  */
/* OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE      */
/* OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.                              */
/******************************************************************************/

#include "jolt_wrapper.h"

#include <Jolt/Jolt.h>
#include <Jolt/RegisterTypes.h>
#include <Jolt/Core/Factory.h>
#include <Jolt/Core/TempAllocator.h>
#include <Jolt/Core/JobSystemThreadPool.h>
#include <Jolt/Physics/PhysicsSettings.h>
#include <Jolt/Physics/PhysicsSystem.h>
#include <Jolt/Physics/Collision/Shape/BoxShape.h>
#include <Jolt/Physics/Collision/Shape/SphereShape.h>
#include <Jolt/Physics/Collision/Shape/CapsuleShape.h>
#include <Jolt/Physics/Collision/Shape/CylinderShape.h>
#include <Jolt/Physics/Collision/Shape/TaperedCapsuleShape.h>
#include <Jolt/Physics/Body/BodyCreationSettings.h>
#include <Jolt/Physics/Body/BodyActivationListener.h>
#include <Jolt/Physics/Collision/RayCast.h>
#include <Jolt/Physics/Collision/CastResult.h>
#include <Jolt/Physics/Collision/CollisionCollectorImpl.h>
#include <Jolt/Physics/Collision/ShapeCast.h>
#include <Jolt/Physics/Collision/NarrowPhaseQuery.h>

JPH_SUPPRESS_WARNINGS

using namespace JPH;


/* Object layers: 0 = non-moving (static), 1 = moving (dynamic). */
namespace Layers {
	static constexpr ObjectLayer NON_MOVING = 0;
	static constexpr ObjectLayer MOVING     = 1;
	static constexpr ObjectLayer NUM_LAYERS = 2;
}

/* Broadphase layers mirror the object layers. */
namespace BPLayers {
	static constexpr BroadPhaseLayer NON_MOVING(0);
	static constexpr BroadPhaseLayer MOVING(1);
	static constexpr uint NUM_LAYERS = 2;
}

class BPLayerImpl final : public BroadPhaseLayerInterface {
public:
	BPLayerImpl() {
		mObjToBP[Layers::NON_MOVING] = BPLayers::NON_MOVING;
		mObjToBP[Layers::MOVING]     = BPLayers::MOVING;
	}
	virtual uint GetNumBroadPhaseLayers() const override {
		return BPLayers::NUM_LAYERS;
	}
	virtual BroadPhaseLayer GetBroadPhaseLayer(
		ObjectLayer layer) const override
	{
		return mObjToBP[layer];
	}
#if defined(JPH_EXTERNAL_PROFILE) || defined(JPH_PROFILE_ENABLED)
	virtual const char* GetBroadPhaseLayerName(
		BroadPhaseLayer layer) const override
	{
		return (layer == BPLayers::NON_MOVING) ? "NON_MOVING" : "MOVING";
	}
#endif
private:
	BroadPhaseLayer mObjToBP[Layers::NUM_LAYERS];
};

class ObjVsBPFilter final : public ObjectVsBroadPhaseLayerFilter {
public:
	virtual bool ShouldCollide(
		ObjectLayer obj, BroadPhaseLayer bp) const override
	{
		if (obj == Layers::NON_MOVING)
			return bp == BPLayers::MOVING;
		return true;
	}
};

class ObjLayerPairFilter final : public ObjectLayerPairFilter {
public:
	virtual bool ShouldCollide(
		ObjectLayer a, ObjectLayer b) const override
	{
		if (a == Layers::NON_MOVING)
			return b == Layers::MOVING;
		return true;
	}
};

/* Internal state bundled together to keep the Go-visible handle opaque. */
struct jolt_PhysicsSystem {
	TempAllocatorImpl  tempAlloc;
	JobSystemThreadPool jobSystem;
	BPLayerImpl         bpLayer;
	ObjVsBPFilter       objVsBP;
	ObjLayerPairFilter  objPair;
	PhysicsSystem       world;

	jolt_PhysicsSystem(int maxBodies)
		: tempAlloc(10 * 1024 * 1024)
		, jobSystem(cMaxPhysicsJobs, cMaxPhysicsBarriers,
		            (int)std::thread::hardware_concurrency() - 1)
	{
		world.Init(
			maxBodies,
			0,       /* numBodyMutexes — 0 = auto */
			65536,   /* maxBodyPairs */
			10240,   /* maxContactConstraints */
			bpLayer, objVsBP, objPair);
	}
};

static bool sJoltInitialized = false;

static void ensureInit() {
	if (!sJoltInitialized) {
		RegisterDefaultAllocator();
		Factory::sInstance = new Factory();
		RegisterTypes();
		sJoltInitialized = true;
	}
}

/* Build a reference-counted shape from the parameters passed by Go. */
static ShapeRefC buildShape(
	int   type,
	float he_x, float he_y, float he_z,
	float radius, float height,
	float /*n_x*/, float /*n_y*/, float /*n_z*/, float /*constant*/)
{

	switch (type) {
	case JOLT_SHAPE_BOX:
		return new BoxShape(Vec3(he_x, he_y, he_z));
	case JOLT_SHAPE_SPHERE:
		return new SphereShape(radius);
	case JOLT_SHAPE_CAPSULE:
		return new CapsuleShape(height * 0.5f, radius);
	case JOLT_SHAPE_CYLINDER:
		return new CylinderShape(height * 0.5f, he_x);
	case JOLT_SHAPE_CONE:
	{
		/* Jolt has no built-in cone; approximate with a tapered capsule
       (top radius ≈ 0, bottom radius = cone base radius). */
		TaperedCapsuleShapeSettings settings(height * 0.5f, 0.001f, he_x);
		auto result = settings.Create();
		return result.Get();
	}
	default:
		return new BoxShape(Vec3(he_x, he_y, he_z));
	}
}

extern "C" {

jolt_PhysicsSystem* jolt_create_system(int max_bodies) {
	ensureInit();
	return new jolt_PhysicsSystem(max_bodies > 0 ? max_bodies : 1024);
}

void jolt_destroy_system(jolt_PhysicsSystem* sys) {
	delete sys;
}

void jolt_set_gravity(jolt_PhysicsSystem* sys, float x, float y, float z) {
	sys->world.SetGravity(Vec3(x, y, z));
}

void jolt_step(jolt_PhysicsSystem* sys, float dt) {
	sys->world.Update(dt, 1, &sys->tempAlloc, &sys->jobSystem);
}

uint32_t jolt_add_body(jolt_PhysicsSystem* sys,
	int   shape_type,
	float he_x,  float he_y,  float he_z,
	float radius, float height,
	float n_x,   float n_y,   float n_z,  float constant,
	float mass,  float friction,
	float px,    float py,    float pz,
	float qx,    float qy,    float qz,   float qw)
{
	ShapeRefC shape = buildShape(shape_type,
		he_x, he_y, he_z, radius, height, n_x, n_y, n_z, constant);
	if (shape == nullptr)
		return BodyID::cInvalidBodyID;

	EMotionType motionType =
		(mass == 0.0f) ? EMotionType::Static : EMotionType::Dynamic;
	ObjectLayer layer =
		(mass == 0.0f) ? Layers::NON_MOVING : Layers::MOVING;

	BodyCreationSettings settings(
		shape,
		RVec3(px, py, pz),
		Quat(qx, qy, qz, qw),
		motionType,
		layer);
	settings.mFriction = friction;
	if (mass > 0.0f) {
		settings.mOverrideMassProperties =
			EOverrideMassProperties::CalculateInertia;
		settings.mMassPropertiesOverride.mMass = mass;
	}

	BodyInterface& bi = sys->world.GetBodyInterface();
	Body* body = bi.CreateBody(settings);
	if (body == nullptr)
		return BodyID::cInvalidBodyID;
	bi.AddBody(body->GetID(), EActivation::Activate);
	return body->GetID().GetIndexAndSequenceNumber();
}

void jolt_remove_body(jolt_PhysicsSystem* sys, uint32_t body_id) {
	BodyID id(body_id);
	BodyInterface& bi = sys->world.GetBodyInterface();
	bi.RemoveBody(id);
	bi.DestroyBody(id);
}

void jolt_apply_force(jolt_PhysicsSystem* sys, uint32_t body_id,
	float fx, float fy, float fz,
	float px, float py, float pz)
{
	BodyInterface& bi = sys->world.GetBodyInterface();
	bi.AddForce(BodyID(body_id), Vec3(fx, fy, fz), RVec3(px, py, pz));
}

void jolt_apply_impulse(jolt_PhysicsSystem* sys, uint32_t body_id,
	float fx, float fy, float fz,
	float px, float py, float pz)
{
	BodyInterface& bi = sys->world.GetBodyInterface();
	bi.AddImpulse(BodyID(body_id), Vec3(fx, fy, fz), RVec3(px, py, pz));
}

void jolt_get_active_transforms(jolt_PhysicsSystem* sys,
	uint32_t* out_ids,
	float*    out_positions,
	float*    out_rotations,
	int       max_capacity,
	int*      out_count)
{
	Array<BodyID> active;
	sys->world.GetActiveBodies(EBodyType::RigidBody, active);

	BodyInterface& bi = sys->world.GetBodyInterface();
	int count = 0;
	for (const BodyID& id : active) {
		if (count >= max_capacity)
			break;
		RVec3 pos;
		Quat  rot;
		bi.GetPositionAndRotation(id, pos, rot);

		out_ids[count]             = id.GetIndexAndSequenceNumber();
		out_positions[count * 3]   = (float)pos.GetX();
		out_positions[count * 3+1] = (float)pos.GetY();
		out_positions[count * 3+2] = (float)pos.GetZ();
		out_rotations[count * 4]   = rot.GetX();
		out_rotations[count * 4+1] = rot.GetY();
		out_rotations[count * 4+2] = rot.GetZ();
		out_rotations[count * 4+3] = rot.GetW();
		count++;
	}
	*out_count = count;
}

jolt_HitResult jolt_raycast(jolt_PhysicsSystem* sys,
	float fx, float fy, float fz,
	float tx, float ty, float tz)
{
	jolt_HitResult result = {};
	RRayCast ray{
		RVec3(fx, fy, fz),
		Vec3(tx - fx, ty - fy, tz - fz)};
	RayCastResult hit;
	if (sys->world.GetNarrowPhaseQuery().CastRay(ray, hit)) {
		RVec3 point = ray.GetPointOnRay(hit.mFraction);
		result.point[0] = (float)point.GetX();
		result.point[1] = (float)point.GetY();
		result.point[2] = (float)point.GetZ();
		BodyLockRead lock(sys->world.GetBodyLockInterface(), hit.mBodyID);
		if (lock.Succeeded()) {
			Vec3 n = lock.GetBody().GetWorldSpaceSurfaceNormal(
				hit.mSubShapeID2, point);
			result.normal[0] = n.GetX();
			result.normal[1] = n.GetY();
			result.normal[2] = n.GetZ();
		}
		result.body_id = hit.mBodyID.GetIndexAndSequenceNumber();
		result.valid   = 1;
	}
	return result;
}

jolt_HitResult jolt_sphere_sweep(jolt_PhysicsSystem* sys,
	float fx, float fy, float fz,
	float tx, float ty, float tz,
	float radius)
{
	jolt_HitResult result = {};
	SphereShape sphere(radius);
	RShapeCast cast = RShapeCast::sFromWorldTransform(
		&sphere,
		Vec3::sReplicate(1.0f),
		RMat44::sTranslation(RVec3(fx, fy, fz)),
		Vec3(tx - fx, ty - fy, tz - fz));
	ShapeCastSettings settings;
	ClosestHitCollisionCollector<CastShapeCollector> collector;
	sys->world.GetNarrowPhaseQuery().CastShape(
		cast, settings, RVec3::sZero(), collector);
	if (collector.HadHit()) {
		const ShapeCastResult& hit = collector.mHit;
		RVec3 point = cast.GetPointOnRay(hit.mFraction);
		result.point[0]  = (float)point.GetX();
		result.point[1]  = (float)point.GetY();
		result.point[2]  = (float)point.GetZ();
		Vec3 n = hit.mPenetrationAxis.NormalizedOr(Vec3::sZero());
		result.normal[0] = n.GetX();
		result.normal[1] = n.GetY();
		result.normal[2] = n.GetZ();
		result.body_id   = hit.mBodyID2.GetIndexAndSequenceNumber();
		result.valid     = 1;
	}
	return result;
}

} /* extern "C" */
